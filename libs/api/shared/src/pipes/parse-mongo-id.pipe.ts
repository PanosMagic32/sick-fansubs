import { BadRequestException, Injectable, PipeTransform } from '@nestjs/common';
import { Types } from 'mongoose';

/**
 * Validates that a route parameter is a valid MongoDB ObjectId.
 * Returns 400 Bad Request for invalid IDs instead of letting
 * Mongoose throw a CastError (which becomes a 500 Internal Server Error).
 */
@Injectable()
export class ParseMongoIdPipe implements PipeTransform<string, string> {
  transform(value: string): string {
    if (!Types.ObjectId.isValid(value)) {
      throw new BadRequestException('Μη έγκυρο αναγνωριστικό.');
    }
    return value;
  }
}
